with vip as (
    select id from departments where ?
)
select u.id, u.name
from users u
join vip on vip.id = u.department_id
where 1 = 1
and u.name = ?
and u.id in (?, ?, ?)
order by u.name, u.id
