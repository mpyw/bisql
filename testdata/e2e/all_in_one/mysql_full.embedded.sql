with active as (
    select id from acct where flag = true
)
select /*+ MAX_EXECUTION_TIME(2000) */  p.id, p.name
from person p
join active a on a.id = p.id
where
 name = 'SCOTT'
and p.id in (1, 2)
order by name, id desc
