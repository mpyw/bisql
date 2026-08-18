with active_depts as (
    select id from departments where $1
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
 and u.age >= 0

and (u.department_id, u.status) in (($2, $3))
 and u.name like $4
order by  u.id
limit 5
