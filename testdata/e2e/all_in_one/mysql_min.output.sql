with vip as (
    select id from departments where ?
)
select u.id, u.name
from users u
join vip on vip.id = u.department_id
where 1 = 1


order by  u.id
