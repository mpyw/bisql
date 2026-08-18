with active_depts as (
    select id from departments where @p1
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
and u.age >= 18
and u.id in (@p2, @p3, @p4)
and (u.department_id, u.status) in ((@p5, @p6))
and u.name like @p7 and u.name like @p8
order by u.name, u.id
limit 50
