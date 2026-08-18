-- Fetch audit-log rows for a set of composite (user_id, action) keys in one round trip.
--
-- SQLite allows a row-value `IN` only against a subquery, not a bare tuple list, so the key set
-- is built as a set: a zero-row `select ... where 1 = 0` seed with one `union all select` per
-- key. `union all` is the leading connector, so an empty key set renders just the seed — an
-- empty subquery that matches nothing, no special-casing required.
select a.id, a.user_id, a.action, a.created_at
from audit_logs a
where (a.user_id, a.action) in (
    select null as user_id, null as action where 1 = 0
    /*%for k in keys*/ union all select /*k.userId*/0, /*k.action*/''/*%end*/
)
order by a.id
