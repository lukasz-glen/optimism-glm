package proofs

import (
	"math/big"
	"testing"

	"github.com/ethereum-optimism/optimism/op-chain-ops/genesis"
	actionsHelpers "github.com/ethereum-optimism/optimism/op-e2e/actions/helpers"
	"github.com/ethereum-optimism/optimism/op-e2e/actions/proofs/helpers"
	"github.com/ethereum-optimism/optimism/op-e2e/bindings"
	"github.com/ethereum-optimism/optimism/op-service/predeploys"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// This test runs through the migration from Isthmus to Jovian
// for a chain already running nonzero operator fee scalars.
// It establishes that no special logic is in place to automatically reset
// the scalars, and fees are therefore expected to vastly increase
// under the new Jovian formula.
func Test_ProgramAction_OperatorFeeFixTransition(gt *testing.T) {
	type testCase int64

	const (
		NormalTx testCase = iota
	)

	run := func(gt *testing.T, testCfg *helpers.TestCfg[testCase]) {
		t := actionsHelpers.NewDefaultTesting(gt)
		deployConfigOverrides := func(dp *genesis.DeployConfig) {
			dp.L2GenesisJovianTimeOffset = ptr(hexutil.Uint64(15))
		}

		var testOperatorFeeScalar uint32
		var testOperatorFeeConstant uint64

		testOperatorFeeScalar = 100e6
		testOperatorFeeConstant = 0

		env := helpers.NewL2FaultProofEnv(t, testCfg, helpers.NewTestParams(), helpers.NewBatcherCfg(), deployConfigOverrides)

		balanceAt := func(a common.Address) *big.Int {
			t.Helper()
			bal, err := env.Engine.EthClient().BalanceAt(t.Ctx(), a, nil)
			require.NoError(t, err)
			return bal
		}

		getCurrentBalances := func() (alice *big.Int, l1FeeVault *big.Int, baseFeeVault *big.Int, sequencerFeeVault *big.Int, operatorFeeVault *big.Int) {
			alice = balanceAt(env.Alice.Address())
			l1FeeVault = balanceAt(predeploys.L1FeeVaultAddr)
			baseFeeVault = balanceAt(predeploys.BaseFeeVaultAddr)
			sequencerFeeVault = balanceAt(predeploys.SequencerFeeVaultAddr)
			operatorFeeVault = balanceAt(predeploys.OperatorFeeVaultAddr)

			return alice, l1FeeVault, baseFeeVault, sequencerFeeVault, operatorFeeVault
		}

		sysCfgContract, err := bindings.NewSystemConfig(env.Sd.RollupCfg.L1SystemConfigAddress, env.Miner.EthClient())
		require.NoError(t, err)

		sysCfgOwner, err := bind.NewKeyedTransactorWithChainID(env.Dp.Secrets.Deployer, env.Sd.RollupCfg.L1ChainID)
		require.NoError(t, err)

		// Update the operator fee parameters
		_, err = sysCfgContract.SetOperatorFeeScalars(sysCfgOwner, testOperatorFeeScalar, testOperatorFeeConstant)
		require.NoError(t, err)

		env.Miner.ActL1StartBlock(12)(t)
		env.Miner.ActL1IncludeTx(env.Dp.Addresses.Deployer)(t)
		env.Miner.ActL1EndBlock(t)

		// sequence L2 blocks, and submit with new batcher
		env.Sequencer.ActL1HeadSignal(t)
		env.Sequencer.ActBuildToL1Head(t)
		env.BatchAndMine(t)

		env.Sequencer.ActL1HeadSignal(t)

		var aliceInitialBalance *big.Int
		var operatorFeeVaultInitialBalance *big.Int

		var receipt *types.Receipt

		aliceInitialBalance, _, _, _, operatorFeeVaultInitialBalance = getCurrentBalances()

		require.Equal(t, operatorFeeVaultInitialBalance.Sign(), 0)

		// Send an L2 tx
		env.Sequencer.ActL2StartBlock(t)
		env.Alice.L2.ActResetTxOpts(t)
		env.Alice.L2.ActSetTxToAddr(&env.Dp.Addresses.Bob)(t)
		env.Alice.L2.ActMakeTx(t)
		env.Engine.ActL2IncludeTx(env.Alice.Address())(t)
		env.Sequencer.ActL2EndBlock(t)

		receipt = env.Alice.L2.LastTxReceipt(t)

		// Check that the operator fee was applied
		require.Equal(t, testOperatorFeeScalar, uint32(*receipt.OperatorFeeScalar))
		require.Equal(t, testOperatorFeeConstant, *receipt.OperatorFeeConstant)

		// Isthmus formula: (gasUsed * operatorFeeScalar / 1e6) + operatorFeeConstant
		expectedOperatorFeeIsthmus := new(big.Int).Add(
			new(big.Int).Div(
				new(big.Int).Mul(
					new(big.Int).SetUint64(receipt.GasUsed),
					new(big.Int).SetUint64(uint64(testOperatorFeeScalar)),
				),
				new(big.Int).SetUint64(1e6),
			),
			new(big.Int).SetUint64(testOperatorFeeConstant),
		)

		aliceFinalBalance, _, _, _, operatorFeeVaultFinalBalance := getCurrentBalances()

		require.Equal(t,
			expectedOperatorFeeIsthmus,
			new(big.Int).Sub(operatorFeeVaultFinalBalance, operatorFeeVaultInitialBalance),
		)

		require.True(t, aliceFinalBalance.Cmp(aliceInitialBalance) < 0, "Alice's balance should decrease")

		// now wind forward to jovian

		env.Sequencer.ActBuildL2ToJovian(t)

		// reset accounting
		aliceInitialBalance, _, _, _, operatorFeeVaultInitialBalance = getCurrentBalances()

		// Send an L2 tx
		env.Sequencer.ActL2StartBlock(t)
		env.Alice.L2.ActResetTxOpts(t)
		env.Alice.L2.ActSetTxToAddr(&env.Dp.Addresses.Bob)(t)
		env.Alice.L2.ActMakeTx(t)
		env.Engine.ActL2IncludeTx(env.Alice.Address())(t)
		env.Sequencer.ActL2EndBlock(t)

		receipt = env.Alice.L2.LastTxReceipt(t)

		// Jovian formula: (gasUsed * operatorFeeScalar * 100) + operatorFeeConstant
		expectedOperatorFeeJovian := new(big.Int).Add(
			new(big.Int).Mul(
				new(big.Int).Mul(
					new(big.Int).SetUint64(receipt.GasUsed),
					new(big.Int).SetUint64(uint64(testOperatorFeeScalar)),
				),
				new(big.Int).SetUint64(100),
			),
			new(big.Int).SetUint64(testOperatorFeeConstant),
		)

		aliceFinalBalance, _, _, _, operatorFeeVaultFinalBalance = getCurrentBalances()

		require.Equal(t,
			expectedOperatorFeeJovian,
			new(big.Int).Sub(operatorFeeVaultFinalBalance, operatorFeeVaultInitialBalance),
		)

		require.Equal(t, uint64(1e8), big.NewInt(0).Div(expectedOperatorFeeJovian, expectedOperatorFeeIsthmus).Uint64())
		env.BatchAndMine(t)
		env.Sequencer.ActL1HeadSignal(t)
		env.Sequencer.ActL2PipelineFull(t)

		l2SafeHead := env.Engine.L2Chain().CurrentSafeBlock()

		env.RunFaultProofProgramFromGenesis(t, l2SafeHead.Number.Uint64(), testCfg.CheckResult, testCfg.InputParams...)
	}

	matrix := helpers.NewMatrix[testCase]()
	matrix.AddDefaultTestCasesWithName("NormalTx", NormalTx, helpers.NewForkMatrix(helpers.Isthmus), run)
	matrix.Run(gt)
}
