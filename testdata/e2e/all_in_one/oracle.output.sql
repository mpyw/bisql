with active as (
    select id from acct where flag = :1
)
select /*+ MAX_EXECUTION_TIME(2000) */  p.id
from person p
join active a on a.id = p.id
where
 name = :2
and p.id in (:3, :4)
