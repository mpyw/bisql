with vip as (
    select id from departments where @p1
)
select u.id, u.name
from users u
join vip on vip.id = u.department_id
where 1 = 1
and u.name = @p2
and u.id in (@p3, @p4, @p5)
order by u.name, u.id
