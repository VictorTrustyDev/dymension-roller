package celestia

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os/exec"
	"path/filepath"
	"strings"

	cosmossdkmath "cosmossdk.io/math"
	cosmossdktypes "github.com/cosmos/cosmos-sdk/types"

	"github.com/dymensionxyz/roller/cmd/consts"
	"github.com/dymensionxyz/roller/cmd/utils"
	globalutils "github.com/dymensionxyz/roller/utils"
	"github.com/dymensionxyz/roller/utils/bash"
	"github.com/dymensionxyz/roller/utils/config"
	"github.com/dymensionxyz/roller/utils/config/tomlconfig"
)

var lcMinBalance = big.NewInt(1)

type Celestia struct {
	Root            string
	rpcEndpoint     string
	metricsEndpoint string
	RPCPort         string
	NamespaceID     string
}

func NewCelestia(home string) *Celestia {
	return &Celestia{
		Root: home,
	}
}

func (c *Celestia) GetPrivateKey() (string, error) {
	exportKeyCmd := c.GetExportKeyCmd()
	out, err := bash.ExecCommandWithStdErr(exportKeyCmd)
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

func (c *Celestia) SetMetricsEndpoint(endpoint string) {
	c.metricsEndpoint = endpoint
}

type BalanceResponse struct {
	Result cosmossdktypes.Coin `json:"result"`
}

func (c *Celestia) GetStatus(rlpCfg config.RollappConfig) string {
	args := []string{
		"state",
		"balance",
		"--node.store",
		filepath.Join(c.Root, consts.ConfigDirName.DALightNode),
	}
	output, err := exec.Command(consts.Executables.Celestia, args...).Output()
	if err != nil {
		return "Stopped, Restarting..."
	}

	var resp BalanceResponse
	err = json.Unmarshal(output, &resp)
	if err != nil {
		return "Stopped, Restarting..."
	}

	if resp.Result.Amount != cosmossdkmath.NewInt(0) {
		return "active"
	}
	// if strings.TrimSpace(resp.Result.Amount) != 0 {
	// 	return "active"
	// }

	return "Stopped, Restarting..."
}

func (c *Celestia) GetRootDirectory() string {
	return c.Root
}

func (c *Celestia) getRPCPort() string {
	if c.RPCPort != "" {
		return c.RPCPort
	}
	port, err := globalutils.GetKeyFromTomlFile(
		filepath.Join(c.Root, consts.ConfigDirName.DALightNode, "config.toml"),
		"RPC.Port",
	)
	if err != nil {
		panic(err)
	}
	c.RPCPort = port
	return port
}

func (c *Celestia) GetLightNodeEndpoint() string {
	return fmt.Sprintf("http://localhost:%s", c.getRPCPort())
}

// GetDAAccountAddress implements datalayer.DataLayer.
// FIXME: should be loaded once and cached
func (c *Celestia) GetDAAccountAddress() (*utils.KeyInfo, error) {
	daKeysDir := filepath.Join(c.Root, consts.ConfigDirName.DALightNode, consts.KeysDirName)
	cmd := exec.Command(
		consts.Executables.CelKey, "show", c.GetKeyName(), "--node.type", "light", "--keyring-dir",
		daKeysDir, "--keyring-backend", "test", "--output", "json",
	)
	output, err := bash.ExecCommandWithStdout(cmd)
	if err != nil {
		return nil, err
	}
	address, err := utils.ParseAddressFromOutput(output)
	return address, err
}

func (c *Celestia) InitializeLightNodeConfig() (string, error) {
	raCfg, err := tomlconfig.LoadRollerConfig(c.Root)
	if err != nil {
		return "", err
	}

	initLightNodeCmd := exec.Command(
		consts.Executables.Celestia, "light", "init",
		"--p2p.network",
		string(raCfg.DA.ID),
		"--node.store", filepath.Join(c.Root, consts.ConfigDirName.DALightNode),
	)
	// err := initLightNodeCmd.Run()
	out, err := bash.ExecCommandWithStdout(initLightNodeCmd)
	if err != nil {
		return "", err
	}

	mnemonic := extractMnemonic(out.String())

	return mnemonic, nil
}

func extractMnemonic(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	mnemonicLineFound := false
	var mnemonicLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if mnemonicLineFound {
			// Collect all subsequent lines as part of the mnemonic
			mnemonicLines = append(mnemonicLines, line)
		}
		if strings.HasPrefix(line, "MNEMONIC") {
			mnemonicLineFound = true
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading output:", err)
		return ""
	}

	return strings.Join(mnemonicLines, " ")
}

func (c *Celestia) getDAAccData(home string) (*utils.AccountData, error) {
	celAddress, err := c.GetDAAccountAddress()
	if err != nil {
		return nil, err
	}

	// TODO: refactor to support multiple DA chains
	raCfg, err := tomlconfig.LoadRollerConfig(home)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(
		consts.Executables.CelestiaApp,
		"q",
		"bank",
		"balances",
		celAddress.Address,
		"--node",
		raCfg.DA.RpcUrl,
		"--chain-id",
		string(raCfg.DA.ID),
		"-o", "json",
	)

	output, err := bash.ExecCommandWithStdout(cmd)
	if err != nil {
		return nil, err
	}
	b := bytes.NewBuffer(output.Bytes())

	balance, err := utils.ParseBalanceFromResponse(
		*b,
		consts.Denoms.Celestia,
	)
	if err != nil {
		return nil, err
	}
	return &utils.AccountData{
		Address: celAddress.Address,
		Balance: balance,
	}, nil
}

func (c *Celestia) GetDAAccData(cfg config.RollappConfig) ([]utils.AccountData, error) {
	celAddress, err := c.getDAAccData(c.Root)
	if err != nil {
		return nil, err
	}
	if celAddress == nil {
		return nil, fmt.Errorf("failed to get DA account data")
	}
	return []utils.AccountData{*celAddress}, err
}

func (c *Celestia) GetKeyName() string {
	return consts.KeysIds.Celestia
}

func (c *Celestia) GetExportKeyCmd() *exec.Cmd {
	return utils.GetExportKeyCmdBinary(
		c.GetKeyName(),
		filepath.Join(c.Root, consts.ConfigDirName.DALightNode, "keys"),
		consts.Executables.CelKey,
	)
}

func (c *Celestia) CheckDABalance() ([]utils.NotFundedAddressData, error) {
	accData, err := c.getDAAccData(c.Root)
	if err != nil {
		return nil, err
	}

	raCfg, err := tomlconfig.LoadRollerConfig(c.Root)
	if err != nil {
		return nil, err
	}

	var insufficientBalances []utils.NotFundedAddressData
	if accData.Balance.Amount.Cmp(lcMinBalance) < 0 {
		insufficientBalances = append(
			insufficientBalances, utils.NotFundedAddressData{
				Address:         accData.Address,
				CurrentBalance:  accData.Balance.Amount,
				RequiredBalance: lcMinBalance,
				KeyName:         c.GetKeyName(),
				Denom:           consts.Denoms.Celestia,
				Network:         string(raCfg.DA.ID),
			},
		)
	}

	return insufficientBalances, nil
}

func (c *Celestia) GetStartDACmd() *exec.Cmd {
	raCfg, err := tomlconfig.LoadRollerConfig(c.Root)
	if err != nil {
		return nil
	}

	args := []string{
		"light", "start",
		"--core.ip", raCfg.DA.StateNode,
		"--node.store", filepath.Join(c.Root, consts.ConfigDirName.DALightNode),
		"--gateway",
		// "--gateway.deprecated-endpoints",
		"--p2p.network", string(raCfg.DA.ID),
	}
	if c.metricsEndpoint != "" {
		args = append(args, "--metrics", "--metrics.endpoint", c.metricsEndpoint)
	}
	startCmd := exec.Command(
		consts.Executables.Celestia, args...,
	)
	// startCmd.Env = append(os.Environ(), CUSTOM_ARABICA11_CONFIG)
	return startCmd
}

func (c *Celestia) SetRPCEndpoint(rpc string) {
	c.rpcEndpoint = rpc
}

func (c *Celestia) GetNetworkName() string {
	return consts.DefaultCelestiaNetwork
}

func (c *Celestia) GetNamespaceID() string {
	return c.NamespaceID
}

func (c *Celestia) getAuthToken(t string, raCfg config.RollappConfig) (string, error) {
	getAuthTokenCmd := exec.Command(
		consts.Executables.Celestia,
		"light",
		"auth",
		t,
		"--p2p.network",
		string(raCfg.DA.ID),
		"--node.store",
		filepath.Join(c.Root, consts.ConfigDirName.DALightNode),
	)
	output, err := bash.ExecCommandWithStdout(getAuthTokenCmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(output.String(), "\n"), nil
}

func (c *Celestia) GetSequencerDAConfig(nt string) string {
	if c.NamespaceID == "" {
		c.NamespaceID = generateRandNamespaceID()
	}
	lcEndpoint := c.GetLightNodeEndpoint()

	var authToken string
	var err error

	raCfg, err := tomlconfig.LoadRollerConfig(c.Root)
	if err != nil {
		return ""
	}

	if nt == consts.NodeType.Sequencer {
		authToken, err = c.getAuthToken(consts.DaAuthTokenType.Admin, raCfg)
	} else if nt == consts.NodeType.FullNode {
		authToken, err = c.getAuthToken(consts.DaAuthTokenType.Read, raCfg)
	} else {
		// TODO: don't panic,return an err
		err := errors.New("invalid node type")
		panic(err)
	}

	if err != nil {
		panic(err)
	}

	return fmt.Sprintf(
		`{"base_url": "%s", "timeout": 60000000000, "gas_prices":0.02, "gas_adjustment": 1.3, "namespace_id":"%s","auth_token":"%s","backoff":{"initial_delay":6000000000,"max_delay":6000000000,"growth_factor":2},"retry_attempts":4,"retry_delay":3000000000}`,
		lcEndpoint,
		c.NamespaceID,
		authToken,
	)
}
