with active as (
    select id from acct where flag = ?
)
select p.id
from person p
join active a on a.id = p.id
where 1 = 1
