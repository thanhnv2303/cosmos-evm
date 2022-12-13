package main

import (
	"errors"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/evmos/ethermint/crypto/hd"
	ethermint "github.com/evmos/ethermint/types"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/simapp/params"
	"github.com/cosmos/cosmos-sdk/snapshots"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	"github.com/evmos/ethermint/encoding"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	dbm "github.com/tendermint/tm-db"
	"io"
	"os"
	"path/filepath"

	sdkserver "github.com/cosmos/cosmos-sdk/server"

	tmlog "github.com/tendermint/tendermint/libs/log"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	ethermintclient "github.com/evmos/ethermint/client"
	cmdcfg "github.com/evmos/ethermint/cmd/config"
	ethermintserver "github.com/evmos/ethermint/server"
	servercfg "github.com/evmos/ethermint/server/config"
	"github.com/ignite-hq/cli/ignite/pkg/cosmoscmd"
	"nvt/app"
)

func main() {
	setupConfig()
	cmdcfg.RegisterDenoms()
	encodingConfig := encoding.MakeConfig(app.ModuleBasics)
	initClientCtx := client.Context{}.
		WithCodec(encodingConfig.Marshaler).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(types.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastBlock).
		WithHomeDir(app.DefaultNodeHome).
		WithKeyringOptions(hd.EthSecp256k1Option()).
		WithViper("ETHERMINT")

	rootCmd, _ := cosmoscmd.NewRootCmd(
		app.Name,
		app.AccountAddressPrefix,
		app.DefaultNodeHome,
		app.Name,
		app.ModuleBasics,
		app.New,
		// this line is used by starport scaffolding # root/arguments
	)
	//oldPersistentPreRunE := rootCmd.PersistentPreRun
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, l []string) error {
		// set the default command outputs
		cmd.SetOut(cmd.OutOrStdout())
		cmd.SetErr(cmd.ErrOrStderr())

		//oldPersistentPreRunE(cmd, l)
		//if err != nil {
		//	return err
		//}
		initClientCtx, err := client.ReadPersistentCommandFlags(initClientCtx, cmd.Flags())
		if err != nil {
			return err
		}

		initClientCtx, err = config.ReadFromClientConfig(initClientCtx)
		if err != nil {
			return err
		}

		if err := client.SetCmdClientContextHandler(initClientCtx, cmd); err != nil {
			return err
		}

		// FIXME: replace AttoPhoton with bond denom
		customAppTemplate, customAppConfig := servercfg.AppConfig(ethermint.AttoPhoton)

		return sdkserver.InterceptConfigsPreRunHandler(cmd, customAppTemplate, customAppConfig)
	}

	//startCommand := &cobra.Command{
	//	Use: "start",
	//}
	rootCmd.Commands()
	for _, command := range rootCmd.Commands() {
		if command.Use == "start" {
			rootCmd.RemoveCommand(command)
			break
		}
	}
	for _, command := range rootCmd.Commands() {
		if command.Use == "keys" {
			rootCmd.RemoveCommand(command)
			break
		}
	}
	//rootCmd.RemoveCommand(startCommand)

	//encodingConfig := encoding.MakeConfig(app.ModuleBasics)
	a := appCreator{encodingConfig}
	ethermintserver.AddCommands(rootCmd, app.DefaultNodeHome, a.newApp, a.appExport, addModuleInitFlags)
	// add keybase, auxiliary RPC, query, and tx child commands
	rootCmd.AddCommand(
		ethermintclient.KeyCommands(app.DefaultNodeHome),
	)

	if err := svrcmd.Execute(rootCmd, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
func addModuleInitFlags(startCmd *cobra.Command) {
	crisis.AddModuleInitFlags(startCmd)
}

func setupConfig() {
	// set the address prefixes
	//config := sdk.GetConfig()
	//cmdcfg.SetBech32Prefixes(config)
	//cmdcfg.SetBip44CoinType(config)
	//config.Seal()
}

type appCreator struct {
	encCfg params.EncodingConfig
}

// newApp is an appCreator
func (a appCreator) newApp(logger tmlog.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
	var cache sdk.MultiStorePersistentCache

	if cast.ToBool(appOpts.Get(sdkserver.FlagInterBlockCache)) {
		cache = store.NewCommitKVStoreCacheManager()
	}

	skipUpgradeHeights := make(map[int64]bool)
	for _, h := range cast.ToIntSlice(appOpts.Get(sdkserver.FlagUnsafeSkipUpgrades)) {
		skipUpgradeHeights[int64(h)] = true
	}

	pruningOpts, err := sdkserver.GetPruningOptionsFromFlags(appOpts)
	if err != nil {
		panic(err)
	}

	snapshotDir := filepath.Join(cast.ToString(appOpts.Get(flags.FlagHome)), "data", "snapshots")
	snapshotDB, err := sdk.NewLevelDB("metadata", snapshotDir)
	if err != nil {
		panic(err)
	}
	snapshotStore, err := snapshots.NewStore(snapshotDB, snapshotDir)
	if err != nil {
		panic(err)
	}

	ethermintApp := app.New(
		logger, db, traceStore, true, skipUpgradeHeights,
		cast.ToString(appOpts.Get(flags.FlagHome)),
		cast.ToUint(appOpts.Get(sdkserver.FlagInvCheckPeriod)),
		cosmoscmd.EncodingConfig(a.encCfg),
		appOpts,
		baseapp.SetPruning(pruningOpts),
		baseapp.SetMinGasPrices(cast.ToString(appOpts.Get(sdkserver.FlagMinGasPrices))),
		baseapp.SetHaltHeight(cast.ToUint64(appOpts.Get(sdkserver.FlagHaltHeight))),
		baseapp.SetHaltTime(cast.ToUint64(appOpts.Get(sdkserver.FlagHaltTime))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOpts.Get(sdkserver.FlagMinRetainBlocks))),
		baseapp.SetInterBlockCache(cache),
		baseapp.SetTrace(cast.ToBool(appOpts.Get(sdkserver.FlagTrace))),
		baseapp.SetIndexEvents(cast.ToStringSlice(appOpts.Get(sdkserver.FlagIndexEvents))),
		baseapp.SetSnapshotStore(snapshotStore),
		baseapp.SetSnapshotInterval(cast.ToUint64(appOpts.Get(sdkserver.FlagStateSyncSnapshotInterval))),
		baseapp.SetSnapshotKeepRecent(cast.ToUint32(appOpts.Get(sdkserver.FlagStateSyncSnapshotKeepRecent))),
	)

	return ethermintApp
}

// appExport creates a new simapp (optionally at a given height)
// and exports state.
func (a appCreator) appExport(
	logger tmlog.Logger, db dbm.DB, traceStore io.Writer, height int64, forZeroHeight bool, jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
) (servertypes.ExportedApp, error) {
	var ethermintApp cosmoscmd.App
	homePath, ok := appOpts.Get(flags.FlagHome).(string)
	if !ok || homePath == "" {
		return servertypes.ExportedApp{}, errors.New("application home not set")
	}

	if height != -1 {
		ethermintApp = app.New(logger, db, traceStore, false, map[int64]bool{}, "", uint(1), cosmoscmd.EncodingConfig(a.encCfg), appOpts)

		if err := ethermintApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	} else {
		ethermintApp = app.New(logger, db, traceStore, true, map[int64]bool{}, "", uint(1), cosmoscmd.EncodingConfig(a.encCfg), appOpts)
	}

	return ethermintApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs)
}
