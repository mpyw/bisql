with active as (
    select id from acct where flag = true
)
select p.id, p.name  /** projected columns */
from person p
join active a on a.id = p.id
where 1 = 1
and name = 'SCOTT'
and p.id in (1, 2)
order by name, id desc 
