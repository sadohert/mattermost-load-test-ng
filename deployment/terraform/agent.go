package terraform

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/mattermost/mattermost-load-test-ng/deployment/terraform/ssh"
	"github.com/mattermost/mattermost-load-test-ng/loadtest"

	"github.com/mattermost/mattermost/server/public/shared/mlog"
)

const dstUsersFilePath = "/home/ubuntu/users.txt"

func (t *Terraform) generateLoadtestAgentConfig() (*loadtest.Config, error) {
	cfg, err := loadtest.ReadConfig("")
	if err != nil {
		return nil, err
	}

	url := getServerURL(t.output, t.config)

	cfg.ConnectionConfiguration.ServerURL = "http://" + url
	cfg.ConnectionConfiguration.WebSocketURL = "ws://" + url
	cfg.ConnectionConfiguration.AdminEmail = t.config.AdminEmail
	cfg.ConnectionConfiguration.AdminPassword = t.config.AdminPassword

	if t.config.UsersFilePath != "" {
		cfg.UsersConfiguration.UsersFilePath = dstUsersFilePath
	}

	return cfg, nil
}

func (t *Terraform) configureAndRunAgents(extAgent *ssh.ExtAgent) error {
	var uploadBinary bool
	var packagePath string
	if strings.HasPrefix(t.config.LoadTestDownloadURL, filePrefix) {
		packagePath = strings.TrimPrefix(t.config.LoadTestDownloadURL, filePrefix)
		info, err := os.Stat(packagePath)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("load-test package path %s has to be a regular file", packagePath)
		}
		uploadBinary = true
	}

	commands := []string{
		"rm -rf mattermost-load-test-ng*",
		"tar xzf tmp.tar.gz",
		"mv mattermost-load-test-ng* mattermost-load-test-ng",
		"rm tmp.tar.gz",
	}
	if !uploadBinary {
		commands = append([]string{"wget -O tmp.tar.gz " + t.config.LoadTestDownloadURL}, commands...)
	}

	// If UsersFilePath is present, split the user credentials among all the agents,
	// so that the logged in users don't clash
	splitFiles := make([][]string, 0, len(t.output.Agents))
	if t.config.UsersFilePath != "" {
		f, err := os.Open(t.config.UsersFilePath)
		if err != nil {
			return fmt.Errorf("error opening UsersFilePath %q", t.config.UsersFilePath)
		}
		scanner := bufio.NewScanner(f)
		for range t.output.Agents {
			splitFiles = append(splitFiles, []string{})
		}
		i := 0
		for scanner.Scan() {
			splitFiles[i] = append(splitFiles[i], scanner.Text())
			i = (i + 1) % len(splitFiles)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading UsersFilePath %q: %w", t.config.UsersFilePath, err)
		}
	}

	for i, val := range t.output.Agents {
		sshc, err := extAgent.NewClient(val.PublicIP)
		if err != nil {
			return err
		}
		mlog.Info("Configuring agent", mlog.String("ip", val.PublicIP))
		if uploadBinary {
			dstFilePath := "/home/ubuntu/tmp.tar.gz"
			mlog.Info("Uploading binary", mlog.String("file", packagePath))
			if out, err := sshc.UploadFile(packagePath, dstFilePath, false); err != nil {
				return fmt.Errorf("error uploading file %q, output: %q: %w", packagePath, out, err)
			}
		}

		cmd := strings.Join(commands, " && ")
		if out, err := sshc.RunCommand(cmd); err != nil {
			return fmt.Errorf("error running command, got output: %q: %w", out, err)
		}

		tpl, err := template.New("").Parse(apiServiceFile)
		if err != nil {
			return fmt.Errorf("could not parse agent service template: %w", err)
		}

		serverCmd := baseAPIServerCmd
		if t.config.EnableAgentFullLogs {
			serverCmd = fmt.Sprintf("/bin/bash -c '%s &>> /home/ubuntu/ltapi.log'", baseAPIServerCmd)
		}

		buf := bytes.NewBufferString("")
		tpl.Execute(buf, serverCmd)

		batch := []uploadInfo{
			{srcData: strings.TrimPrefix(buf.String(), "\n"), dstPath: "/lib/systemd/system/ltapi.service", msg: "Uploading load-test api service file"},
			{srcData: strings.TrimPrefix(clientSysctlConfig, "\n"), dstPath: "/etc/sysctl.conf"},
			{srcData: strings.TrimPrefix(limitsConfig, "\n"), dstPath: "/etc/security/limits.conf"},
			{srcData: strings.TrimPrefix(prometheusNodeExporterConfig, "\n"), dstPath: "/etc/default/prometheus-node-exporter"},
		}

		if t.config.UsersFilePath != "" {
			batch = append(batch, uploadInfo{srcData: strings.Join(splitFiles[i], "\n"), dstPath: dstUsersFilePath, msg: "Uploading list of users credentials"})
		}

		// If SiteURL is set, update /etc/hosts to point to the correct IP
		if t.config.SiteURL != "" {
			output, err := t.Output()
			if err != nil {
				return err
			}

			// The new entry in /etc/hosts will make SiteURL point to:
			// - The first instance's IP if there's a single node
			// - The proxy's IP if there's more than one node
			ip := output.Instances[0].PrivateIP
			if output.HasProxy() {
				ip = output.Proxy.PrivateIP
			}

			proxyHost := fmt.Sprintf("%s %s\n", ip, t.config.SiteURL)
			appHostsFile := fmt.Sprintf(appHosts, proxyHost)

			batch = append(batch, uploadInfo{srcData: appHostsFile, dstPath: "/etc/hosts", msg: "Updating /etc/hosts to point to the correct IP"})
		}

		if err := uploadBatch(sshc, batch); err != nil {
			return fmt.Errorf("batch upload failed: %w", err)
		}

		if out, err := sshc.RunCommand("sudo sysctl -p"); err != nil {
			return fmt.Errorf("error running command, got output: %q: %w", out, err)
		}

		mlog.Info("Starting load-test api server")
		if out, err := sshc.RunCommand("sudo systemctl daemon-reload && sudo service ltapi restart"); err != nil {
			return fmt.Errorf("error running command, got output: %q: %w", out, err)
		}
	}
	return nil
}

func (t *Terraform) initLoadtest(extAgent *ssh.ExtAgent, initData bool) error {
	if len(t.output.Agents) == 0 {
		return errors.New("there are no agents to initialize load-test")
	}
	ip := t.output.Agents[0].PublicIP
	sshc, err := extAgent.NewClient(ip)
	if err != nil {
		return err
	}
	mlog.Info("Generating load-test config")
	cfg, err := t.generateLoadtestAgentConfig()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dstPath := "/home/ubuntu/mattermost-load-test-ng/config/config.json"
	mlog.Info("Uploading updated config file")
	if out, err := sshc.Upload(bytes.NewReader(data), dstPath, false); err != nil {
		return fmt.Errorf("error uploading file, output: %q: %w", out, err)
	}

	if initData && t.config.TerraformDBSettings.ClusterIdentifier == "" {
		mlog.Info("Populating initial data for load-test", mlog.String("agent", ip))
		cmd := fmt.Sprintf("cd mattermost-load-test-ng && ./bin/ltagent init --user-prefix '%s'", t.output.Agents[0].Tags.Name)
		if out, err := sshc.RunCommand(cmd); err != nil {
			// TODO: make this fully atomic. See MM-23998.
			// ltagent init should drop teams and channels before creating them.
			// This needs additional delete actions to be added.
			if strings.Contains(string(out), "with that name already exists") {
				return nil
			}
			return fmt.Errorf("error running ssh command, output: %q, error: %w", out, err)
		}
	}

	return nil
}
