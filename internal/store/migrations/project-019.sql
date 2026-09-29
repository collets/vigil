-- Stage 5.6: record the state a delivery operation was in when reconciliation
-- claimed it.
--
-- A claim writes operations.state='reconciled', so a claim left by a reconcile
-- that died is indistinguishable from a committed closure without the closure
-- marker, and the pre-claim state is not recorded anywhere. Recovering it by
-- inference is wrong in both directions: treating a claim as 'uncertain'
-- silently removes an executing operation's ability to re-execute its effect,
-- while treating it as 'executing' escalates an operation the application had
-- deliberately made observation-only into a re-executable one.
--
-- claimed_from_state is NULL unless a claim is currently held, and records
-- exactly the state the operation had before it. Releasing a claim restores
-- that value, and clearing it returns the operation to whatever it has become.
ALTER TABLE operations ADD COLUMN claimed_from_state TEXT
  CHECK(claimed_from_state IS NULL OR claimed_from_state IN('executing','uncertain'));
