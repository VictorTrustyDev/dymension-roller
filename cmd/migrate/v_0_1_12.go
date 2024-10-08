package migrate

import (
	"path/filepath"

	"github.com/dymensionxyz/roller/cmd/consts"
	datalayer "github.com/dymensionxyz/roller/data_layer"
	"github.com/dymensionxyz/roller/data_layer/avail"
	"github.com/dymensionxyz/roller/sequencer"
	"github.com/dymensionxyz/roller/utils"
	configutils "github.com/dymensionxyz/roller/utils/config"
	"github.com/dymensionxyz/roller/utils/config/tomlconfig"
	"github.com/dymensionxyz/roller/utils/filesystem"
)

type VersionMigratorV0112 struct{}

func (v *VersionMigratorV0112) ShouldMigrate(prevVersion VersionData) bool {
	return prevVersion.Major < 1 && prevVersion.Minor < 2 && prevVersion.Patch < 12
}

func (v *VersionMigratorV0112) PerformMigration(rlpCfg configutils.RollappConfig) error {
	dymintTomlPath := sequencer.GetDymintFilePath(rlpCfg.Home)
	if rlpCfg.DA.Backend == "mock" {
		rlpCfg.DA.Backend = consts.Local
		return tomlconfig.Write(rlpCfg)
	}
	if rlpCfg.DA.Backend == consts.Avail {
		availNewCfgPath := avail.GetCfgFilePath(rlpCfg.Home)
		if err := filesystem.MoveFile(filepath.Join(rlpCfg.Home, avail.ConfigFileName), availNewCfgPath); err != nil {
			return err
		}
	}
	da := datalayer.NewDAManager(rlpCfg.DA.Backend, rlpCfg.Home)
	sequencerDaConfig := da.GetSequencerDAConfig(consts.NodeType.Sequencer)
	if sequencerDaConfig == "" {
		return nil
	}
	if err := utils.UpdateFieldInToml(dymintTomlPath, "da_config", sequencerDaConfig); err != nil {
		return err
	}
	return nil
}
