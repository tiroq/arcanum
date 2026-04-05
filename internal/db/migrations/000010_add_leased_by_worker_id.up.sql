-- Bind each lease to the specific worker that acquired it.
-- This allows Complete() and Fail() to guard on worker identity, preventing
-- stale goroutines (whose lease was reclaimed and re-issued to another worker)
-- from corrupting the state of a job they no longer own.
ALTER TABLE processing_jobs ADD COLUMN leased_by_worker_id TEXT;
