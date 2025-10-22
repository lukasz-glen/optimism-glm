# Optimism-GLM

This is a modified Optimism stack.
The goal is to provide L3 layer with native tokens backed by GLM tokens.

### Changes to the base platform

L2 - no changes, all contracts remain the same

L1 <> L2 - no changes, the structure of events (migration events to L2) and L2 blocks (on L1 side) remain the same

Disputes - no changes, becaues the structure of L2 blocks are not changed

L1:

1. Deployment scripts and other scripts remains almost unchanged, see below.

2. One additional deployment parameter: `glmToken` address.

3. The contract `OptimismPortal2` is changed.

4. Other contracts remain almost unchanged.

5. All contracts are deployed, even if rendered unusable due to the current changes. 
It is to minimize the number of changes in the tools and code.

### General solution

The core L1 contract `OptimismPortal2` is changed to implement the requirements.
The changes are provided where a usesr interacts with the rollup.
Disputes, commitments, and proofs are excluded - remains iunchanged.

The list of changes to the `OptimismPortal2` contract:

1. The function `depositTransaction` always reverts, is unusable.

2. The functions `finalizeWithdrawalTransactions` and `finalizeWithdrawalTransactionExternalProof` always revert, are unusable.

3. The fallback function `receive` always reverts, is unusable.

4. The function `denateETH` always reverts, is unusable.

5. The function `depositGLM` is added.

6. The function `finalizeGLMWithdrawal` is added.

The function `depositGLM` deposits glm tokens in the contract and emits cross-chain event
with the ETH value equals to the deposited GLM amount.

The function `finalizeGLMWithdrawal` accepts any incoming cross-chain message. 
It releases deposited GLM tokens in the amount equals to the ETH value in the message.

It is still possible to attach data. In the case of deposits, data is included in the cross-chain event.
In the case of withdrawals, data is simply logged. 
The target is not called! So data is purely for logging purposes.

A user is reponsible to set proper gas limits as it is with the base optimism.

### Bridge

On L1, only the contract `OptimismPortal2` may be successfully used to migrate tokens.

The contracts `L1CrossDomainMessanger`, `L1ERC721Bridge`, and `L1StandardBridge` are deployed.
But they are functions that are rendered unusable. In consequence they are unusable also.
They are still deployed to minimize changes to the infrastructure.

No ETH tokens are deposited at the contracts `OptimismPortal2` and `ETHLockBox`.
Only GLM tokens are deposited at `OptimismPortal2`. 
Despite that, the contract `ETHLockBox` is depoloyed and connected to `OptimismPortal2`.
`ETHLockBox` cannot donate ETH.

### Deposits and Withdrawals

To deposit GLM tokens a user must: approve GLM to `OptimismPortal2`, call `OptimismPortal2.depositGLM`.
On the L2 side, a target address receives ETH. Any data may be attached.

To withdraw GLM tokens a user must do the following. On the L2 side, the user withdraws ETH tokens
as usual. The L2 blocks must be committed as usual. The dispute period must elapse and the user
sends the withdrawal proof as usual. The last step is different - the user calls `OptimismPortal2.finalizeGLMWithdrawal` to unlock GLM tokens.

