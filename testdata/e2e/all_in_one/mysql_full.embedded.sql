with vip as (
    select id from departments where true
)
select u.id, u.name
from users u
join vip on vip.id = u.department_id
where 1 = 1
and u.name = 'Alice'
and u.id in (1, 2, 3)
order by u.name, u.id
