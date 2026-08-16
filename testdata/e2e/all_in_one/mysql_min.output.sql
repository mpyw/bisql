with active as (
    select id from acct where flag = ?
)
select /*+ MAX_EXECUTION_TIME(2000) */  p.id
from person p
join active a on a.id = p.id
