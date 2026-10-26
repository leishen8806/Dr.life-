// Package ledger is the only production package allowed to mutate ledger
// balances. Callers use reviewed business methods; the generic posting
// primitive remains package-private so callers cannot bypass account-kind,
// ownership, state-machine, or separation-of-duty checks.
//
// Public write contract:
//   - EnsureWallet creates the wallet set identified by its owner and merchant.
//   - Topup requires confirmed bank evidence and keys retries by bank line or
//     manual confirmation.
//   - FreezeHold, SettleHold, RefundHold, DisputeHold, and ResolveDispute own
//     the hold state machine and key retries by hold reference.
//   - RequestWithdrawal, StartReview, ApproveReview, ConfirmPayout,
//     FailWithdrawal, and CancelWithdrawal own the withdrawal state machine;
//     their idempotency derives from the request key or withdrawal record.
//   - StorageFee, DeliveryHold, DeliveryRelease, DeliveryRefund, AdCharge,
//     AdSpend, AdRefund, MemberFee, Sweep, CarrierPay, and BankDiff accept
//     narrow business inputs and derive idempotency keys from their records.
//   - Reverse mirrors an allowed original transaction once and requires a
//     distinct authorizer. Adjust requires two actors and permits only
//     PLATFORM_HELD, SUSPENSE, and PAYABLE_CARRIER accounts.
//   - SnapshotDaily, RunDailyReconciliation, ReleasePayoutFreeze, and the
//     Get/List/Verify methods expose the reconciliation and query contracts.
//     DecryptWithdrawalPayee is limited to the bank-disbursement adapter.
//   - ClaimOutbox leases committed events; callers must finish each batch with
//     MarkSent or Release so a failed delivery remains retryable.
//   - OldestUnsentOutboxAge reports the current committed-event delivery lag.
//
// Invalid input returns ErrInvalidRequest or ErrInvalidAmount. State-machine,
// ownership, insufficient-funds, separation-of-duty, and idempotency failures
// return their dedicated sentinel errors. A successful retry returns the
// original PostResult with Replayed set and does not repeat state changes or
// business outbox events.
//
// Adding a public business method requires a matching action in chapter 6 of
// the database design, an update to TestPublicAPISurface, table-driven posting
// assertions, and coverage in the randomized accounting-invariant sequence.
package ledger
