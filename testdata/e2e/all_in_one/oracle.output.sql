with active as (
    select id from acct where flag = :1
)
select p.id, p.name
from person p
join active a on a.id = p.id
where 1 = 1
and name = :2
and p.id in (:3, :4)
order by name, p.id
