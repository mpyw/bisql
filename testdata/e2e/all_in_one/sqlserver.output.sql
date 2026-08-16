with active as (
    select id from acct where flag = @p1
)
select /*+ MAX_EXECUTION_TIME(2000) */  p.id
from person p
join active a on a.id = p.id
where
 name = @p2
and p.id in (@p3, @p4)
