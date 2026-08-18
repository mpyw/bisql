with active_depts as (
    select id from departments where $1
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
 and u.age >= 18
and u.id in ($2, $3, $4)
and (u.department_id, u.status) in (($5, $6))
and u.name like $7 and u.name like $8
order by u.name, u.id
limit 50
