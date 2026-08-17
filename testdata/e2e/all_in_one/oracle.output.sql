with vip as (
    select id from departments where :1
)
select u.id, u.name
from users u
join vip on vip.id = u.department_id
where 1 = 1
and u.name = :2
and u.id in (:3, :4, :5)
order by u.name, u.id
