package pg

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"
	"github.com/wal-g/wal-g/internal"
	"github.com/wal-g/wal-g/internal/databases/postgres"
	"github.com/wal-g/wal-g/internal/multistorage"
	"github.com/wal-g/wal-g/internal/multistorage/policies"
	"github.com/wal-g/wal-g/utility"
)

const (
	backupListShortDescription = "Prints full list of backups from which recovery is available"
	PrettyFlag                 = "pretty"
	JSONFlag                   = "json"
	DetailFlag                 = "detail"
)

var (
	// backupListCmd represents the backupList command
	backupListCmd = &cobra.Command{
		Use:   "backup-list",
		Short: backupListShortDescription,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			storage, err := postgres.ConfigureMultiStorage(false)
			tracelog.ErrorLogger.FatalOnError(err)

			rootFolder := multistorage.SetPolicies(storage.RootFolder(), policies.UniteAllStorages)
			if targetStorage == "" {
				rootFolder, err = multistorage.UseAllAliveStorages(rootFolder)
			} else {
				rootFolder, err = multistorage.UseSpecificStorage(targetStorage, rootFolder)
			}
			tracelog.ErrorLogger.FatalOnError(err)
			tracelog.InfoLogger.Printf("List backups from storages: %v", multistorage.UsedStorages(rootFolder))

			backupsFolder := rootFolder.GetSubFolder(utility.BaseBackupPath)
			if detail {
				postgres.HandleDetailedBackupList(backupsFolder, pretty, json)
			} else {
				internal.HandleDefaultBackupList(backupsFolder, pretty, json)
			}
		},
	}
	pretty = false
	json   = false
	detail = false
)

func init() {
	Cmd.AddCommand(backupListCmd)

	// TODO: Merge similar backup-list functionality
	// to avoid code duplication in command handlers
	backupListCmd.Flags().BoolVar(&pretty, PrettyFlag, false,
		"Prints more readable output in table format")
	backupListCmd.Flags().BoolVar(&json, JSONFlag, false,
		"Prints output in JSON format, multiline and indented if combined with --pretty flag")
	backupListCmd.Flags().BoolVar(&detail, DetailFlag, false,
		"Prints extra DB-specific backup details")
	backupListCmd.Flags().StringVar(&targetStorage, "target-storage", "",
		targetStorageDescription)
}
