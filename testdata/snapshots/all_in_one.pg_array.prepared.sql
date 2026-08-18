with active_depts as (
    select id from departments where $1
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
and u.status = $2

and u.department_id = $3

 and u.age >= 18
and u.id in ($4, $5, $6)
and u.tags && $7::text[]
and (u.department_id, u.status) in (($8, $9))
 and u.name like $10 and u.name like $11
order by u.name, u.id
limit 50
