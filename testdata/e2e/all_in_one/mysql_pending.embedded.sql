with active_depts as (
    select id from departments where false
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
and u.age >= 13

and (u.department_id, u.status) in ((3, 'banned'))

order by  u.id
limit 10
