-- Code generated from sql/audit_logs/bulk-insert.sql; DO NOT EDIT. Regenerate with `go test ./... -update`.
-- The two-way template with every @include expanded.

-- Bulk-append audit events in one statement.
--
-- A multi-row VALUES has no anchor position — a trailing comma or an empty `values ()` is
-- invalid — so the rows are written as a set instead: a zero-row `select ... where 1 = 0` seed
-- with one `union all select` per event. An empty batch renders just the seed and inserts
-- nothing, so the caller never has to special-case the empty list.
insert into audit_logs (user_id, action)
select 0, '' where 1 = 0
/*%for e in events*/ union all select /*e.userId*/0, /*e.action*/''/*%end*/
