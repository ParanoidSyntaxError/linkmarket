package changeset

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/smartcontractkit/ccip-owner-contracts/pkg/gethwrappers"
	"github.com/smartcontractkit/ccip-owner-contracts/pkg/proposal/mcms"
	"github.com/smartcontractkit/ccip-owner-contracts/pkg/proposal/timelock"

	"github.com/smartcontractkit/chainlink/deployment"
	"github.com/smartcontractkit/chainlink/deployment/common/proposalutils"
)

// MCMSConfig defines timelock duration.
type MCMSConfig struct {
	MinDelay time.Duration
}

type DeployerGroup struct {
	e                 deployment.Environment
	state             CCIPOnChainState
	mcmConfig         *MCMSConfig
	deploymentContext *DeploymentContext
}

type DeploymentContext struct {
	description    string
	transactions   map[uint64][]*types.Transaction
	previousConfig *DeploymentContext
}

func NewDeploymentContext(description string) *DeploymentContext {
	return &DeploymentContext{
		description:    description,
		transactions:   make(map[uint64][]*types.Transaction),
		previousConfig: nil,
	}
}

func (d *DeploymentContext) Fork(description string) *DeploymentContext {
	return &DeploymentContext{
		description:    description,
		transactions:   make(map[uint64][]*types.Transaction),
		previousConfig: d,
	}
}

type DeployerGroupWithContext interface {
	WithDeploymentContext(description string) *DeployerGroup
}

type deployerGroupBuilder struct {
	e         deployment.Environment
	state     CCIPOnChainState
	mcmConfig *MCMSConfig
}

func (d *deployerGroupBuilder) WithDeploymentContext(description string) *DeployerGroup {
	return &DeployerGroup{
		e:                 d.e,
		mcmConfig:         d.mcmConfig,
		state:             d.state,
		deploymentContext: NewDeploymentContext(description),
	}
}

// DeployerGroup is an abstraction that lets developers write their changeset
// without needing to know if it's executed using a DeployerKey or an MCMS proposal.
//
// Example usage:
//
//	deployerGroup := NewDeployerGroup(e, state, mcmConfig)
//	selector := 0
//	# Get the right deployer key for the chain
//	deployer := deployerGroup.GetDeployer(selector)
//	state.Chains[selector].RMNRemote.Curse()
//	# Execute the transaction or create the proposal
//	deployerGroup.Enact("Curse RMNRemote")
func NewDeployerGroup(e deployment.Environment, state CCIPOnChainState, mcmConfig *MCMSConfig) DeployerGroupWithContext {
	return &deployerGroupBuilder{
		e:         e,
		mcmConfig: mcmConfig,
		state:     state,
	}
}

func (d *DeployerGroup) WithDeploymentContext(description string) *DeployerGroup {
	return &DeployerGroup{
		e:                 d.e,
		mcmConfig:         d.mcmConfig,
		state:             d.state,
		deploymentContext: d.deploymentContext.Fork(description),
	}
}

func (d *DeployerGroup) GetDeployer(chain uint64) (*bind.TransactOpts, error) {
	txOpts := d.e.Chains[chain].DeployerKey
	if d.mcmConfig != nil {
		txOpts = deployment.SimTransactOpts()
		txOpts = &bind.TransactOpts{
			From:       d.state.Chains[chain].Timelock.Address(),
			Signer:     txOpts.Signer,
			GasLimit:   txOpts.GasLimit,
			GasPrice:   txOpts.GasPrice,
			Nonce:      txOpts.Nonce,
			Value:      txOpts.Value,
			GasFeeCap:  txOpts.GasFeeCap,
			GasTipCap:  txOpts.GasTipCap,
			Context:    txOpts.Context,
			AccessList: txOpts.AccessList,
			NoSend:     txOpts.NoSend,
		}
	}
	sim := &bind.TransactOpts{
		From:       txOpts.From,
		Signer:     txOpts.Signer,
		GasLimit:   txOpts.GasLimit,
		GasPrice:   txOpts.GasPrice,
		Nonce:      txOpts.Nonce,
		Value:      txOpts.Value,
		GasFeeCap:  txOpts.GasFeeCap,
		GasTipCap:  txOpts.GasTipCap,
		Context:    txOpts.Context,
		AccessList: txOpts.AccessList,
		NoSend:     true,
	}
	oldSigner := sim.Signer

	var startingNonce *big.Int
	if txOpts.Nonce != nil {
		startingNonce = new(big.Int).Set(txOpts.Nonce)
	} else {
		nonce, err := d.e.Chains[chain].Client.PendingNonceAt(context.Background(), txOpts.From)
		if err != nil {
			return nil, fmt.Errorf("could not get nonce for deployer: %w", err)
		}
		startingNonce = new(big.Int).SetUint64(nonce)
	}

	dc := d.deploymentContext
	sim.Signer = func(a common.Address, t *types.Transaction) (*types.Transaction, error) {
		txCount, err := d.getTransactionCount(chain)
		if err != nil {
			return nil, err
		}

		currentNonce := big.NewInt(0).Add(startingNonce, txCount)

		tx, err := oldSigner(a, t)
		if err != nil {
			return nil, err
		}
		dc.transactions[chain] = append(dc.transactions[chain], tx)
		// Update the nonce to consider the transactions that have been sent
		sim.Nonce = big.NewInt(0).Add(currentNonce, big.NewInt(1))
		return tx, nil
	}
	return sim, nil
}

func (d *DeployerGroup) getContextChainInOrder() []*DeploymentContext {
	contexts := make([]*DeploymentContext, 0)
	for c := d.deploymentContext; c != nil; c = c.previousConfig {
		contexts = append(contexts, c)
	}
	slices.Reverse(contexts)
	return contexts
}

func (d *DeployerGroup) getTransactions() map[uint64][]*types.Transaction {
	transactions := make(map[uint64][]*types.Transaction)
	for _, c := range d.getContextChainInOrder() {
		for k, v := range c.transactions {
			transactions[k] = append(transactions[k], v...)
		}
	}
	return transactions
}

func (d *DeployerGroup) getTransactionCount(chain uint64) (*big.Int, error) {
	txs := d.getTransactions()
	return big.NewInt(int64(len(txs[chain]))), nil
}

func (d *DeployerGroup) Enact() (deployment.ChangesetOutput, error) {
	if d.mcmConfig != nil {
		return d.enactMcms()
	}

	return d.enactDeployer()
}

func (d *DeployerGroup) enactMcms() (deployment.ChangesetOutput, error) {
	contexts := d.getContextChainInOrder()
	proposals := make([]timelock.MCMSWithTimelockProposal, 0)
	for _, dc := range contexts {
		batches := make([]timelock.BatchChainOperation, 0)
		for selector, txs := range dc.transactions {
			mcmOps := make([]mcms.Operation, len(txs))
			for i, tx := range txs {
				mcmOps[i] = mcms.Operation{
					To:    *tx.To(),
					Data:  tx.Data(),
					Value: tx.Value(),
				}
			}
			batches = append(batches, timelock.BatchChainOperation{
				ChainIdentifier: mcms.ChainIdentifier(selector),
				Batch:           mcmOps,
			})
		}

		if len(batches) == 0 {
			d.e.Logger.Warnf("No batch was produced from deployment context skipping proposal: %s", dc.description)
			continue
		}

		timelocksPerChain := BuildTimelockAddressPerChain(d.e, d.state)

		proposerMCMSes := BuildProposerPerChain(d.e, d.state)

		prop, err := proposalutils.BuildProposalFromBatches(
			timelocksPerChain,
			proposerMCMSes,
			batches,
			dc.description,
			d.mcmConfig.MinDelay,
		)

		// Update the proposal metadata to incorporate the startingOpCount
		// from the previous proposal
		if len(proposals) > 0 {
			previousProposal := proposals[len(proposals)-1]
			for chain, metadata := range previousProposal.ChainMetadata {
				nextStartingOp := metadata.StartingOpCount + getBatchCountForChain(chain, prop)
				prop.ChainMetadata[chain] = mcms.ChainMetadata{
					StartingOpCount: nextStartingOp,
					MCMAddress:      prop.ChainMetadata[chain].MCMAddress,
				}
			}
		}

		if err != nil {
			return deployment.ChangesetOutput{}, fmt.Errorf("failed to build proposal %w", err)
		}

		proposals = append(proposals, *prop)
	}

	return deployment.ChangesetOutput{
		Proposals: proposals,
	}, nil
}

func getBatchCountForChain(chain mcms.ChainIdentifier, m *timelock.MCMSWithTimelockProposal) uint64 {
	batches := make([]timelock.BatchChainOperation, 0)
	for _, t := range m.Transactions {
		if t.ChainIdentifier == chain {
			batches = append(batches, t)
		}
	}
	return uint64(len(batches))
}

func (d *DeployerGroup) enactDeployer() (deployment.ChangesetOutput, error) {
	contexts := d.getContextChainInOrder()
	for _, c := range contexts {
		for selector, txs := range c.transactions {
			for _, tx := range txs {
				err := d.e.Chains[selector].Client.SendTransaction(context.Background(), tx)
				if err != nil {
					return deployment.ChangesetOutput{}, fmt.Errorf("failed to send transaction: %w", err)
				}
				// TODO how to pass abi here to decode error reason
				_, err = deployment.ConfirmIfNoError(d.e.Chains[selector], tx, err)
				if err != nil {
					return deployment.ChangesetOutput{}, fmt.Errorf("waiting for tx to be mined failed: %w", err)
				}
			}
		}
	}
	return deployment.ChangesetOutput{}, nil
}

func BuildTimelockPerChain(e deployment.Environment, state CCIPOnChainState) map[uint64]*proposalutils.TimelockExecutionContracts {
	timelocksPerChain := make(map[uint64]*proposalutils.TimelockExecutionContracts)
	for _, chain := range e.Chains {
		timelocksPerChain[chain.Selector] = &proposalutils.TimelockExecutionContracts{
			Timelock:  state.Chains[chain.Selector].Timelock,
			CallProxy: state.Chains[chain.Selector].CallProxy,
		}
	}
	return timelocksPerChain
}

func BuildTimelockAddressPerChain(e deployment.Environment, state CCIPOnChainState) map[uint64]common.Address {
	timelocksPerChain := BuildTimelockPerChain(e, state)
	timelockAddressPerChain := make(map[uint64]common.Address)
	for chain, timelock := range timelocksPerChain {
		timelockAddressPerChain[chain] = timelock.Timelock.Address()
	}
	return timelockAddressPerChain
}

func BuildProposerPerChain(e deployment.Environment, state CCIPOnChainState) map[uint64]*gethwrappers.ManyChainMultiSig {
	proposerPerChain := make(map[uint64]*gethwrappers.ManyChainMultiSig)
	for _, chain := range e.Chains {
		proposerPerChain[chain.Selector] = state.Chains[chain.Selector].ProposerMcm
	}
	return proposerPerChain
}
