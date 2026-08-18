with active_depts as (
    select id from departments where true
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
and u.age >= 18
and u.id in (1, 2, 3)
and (u.department_id, u.status) in ((1, 'active'))
and u.name like '%ali%' and u.name like '%bob%'
order by u.name, u.id
limit 50
