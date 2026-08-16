with active as (
    select id from acct where flag = @p1
)
select p.id
from person p
join active a on a.id = p.id
where 1 = 1
and name = @p2
and p.id in (@p3, @p4)
