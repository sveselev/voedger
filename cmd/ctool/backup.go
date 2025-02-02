/*
* Copyright (c) 2023-present Sigma-Soft, Ltd.
* @author Dmitry Molchanovsky
 */

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/robfig/cron/v3"
)

// nolint
func newBackupCmd() *cobra.Command {
	backupNodeCmd := &cobra.Command{
		Use:   "node [<node> <target folder> <path to ssh key>]",
		Short: "Backup db node",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 3 {
				return ErrInvalidNumberOfArguments
			}
			return nil
		},
		RunE: backupNode,
	}

	backupNodeCmd.PersistentFlags().StringVarP(&sshPort, "ssh-port", "p", "22", "SSH port")

	backupCronCmd := &cobra.Command{
		Use:   "cron [<cron event>]",
		Short: "Installation of a backup of schedule",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return ErrInvalidNumberOfArguments
			}
			return nil
		},
		RunE: backupCron,
	}
	backupCronCmd.PersistentFlags().StringVar(&sshKey, "ssh-key", "", "Path to SSH key")
	if err := backupCronCmd.MarkPersistentFlagRequired("ssh-key"); err != nil {
		loggerError(err.Error())
		return nil
	}

	backupCmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup database",
	}

	backupCmd.AddCommand(backupNodeCmd, backupCronCmd)

	return backupCmd

}

// nolint
func validateBackupCronCmd(cmd *cmdType, cluster *clusterType) error {

	if len(cmd.Args) != 2 {
		return ErrInvalidNumberOfArguments
	}

	if _, err := cron.ParseStandard(cmd.Args[1]); err != nil {
		return err
	}

	return nil
}

// nolint
func validateBackupNodeCmd(cmd *cmdType, cluster *clusterType) error {

	if len(cmd.Args) != 4 {
		return ErrInvalidNumberOfArguments
	}

	var err error

	if n := cluster.nodeByHost(cmd.Args[1]); n == nil {
		err = errors.Join(err, fmt.Errorf(errHostNotFoundInCluster, cmd.Args[1], ErrHostNotFoundInCluster))
	}

	if !fileExists(cmd.Args[3]) {
		err = errors.Join(err, fmt.Errorf(errSshKeyNotFound, cmd.Args[3], ErrFileNotFound))
	}

	return err
}

func backupNode(cmd *cobra.Command, args []string) error {
	cluster := newCluster()

	var err error

	if err = mkCommandDirAndLogFile(cmd, cluster); err != nil {
		return err
	}

	loggerInfo("Backup node", strings.Join(args, " "))
	if err = newScriptExecuter("", "").
		run("backupnode.sh", args...); err != nil {
		return err
	}

	return nil
}

func backupCron(cmd *cobra.Command, args []string) error {
	cluster := newCluster()
	if cluster.Draft {
		return ErrClusterConfNotFound
	}

	Cmd := newCmd(ckBackup, append([]string{"cron"}, args...))

	var err error

	if err = Cmd.validate(cluster); err != nil {
		return err
	}

	if err = mkCommandDirAndLogFile(cmd, cluster); err != nil {
		return err
	}

	if err = setCronBackup(cluster, args[0]); err != nil {
		return err
	}

	loggerInfoGreen("Cron schedule set successfully")

	cluster.Cron.Backup = args[0]
	if err = cluster.saveToJSON(); err != nil {
		return err
	}

	return nil
}
