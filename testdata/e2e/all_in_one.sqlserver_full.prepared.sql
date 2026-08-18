with active_depts as (
    select id from departments where @p1
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
and u.status = @p2

and u.department_id = @p3

 and u.age >= 18
and u.id in (@p4, @p5, @p6)

and (u.department_id, u.status) in ((@p7, @p8))
 and u.name like @p9 and u.name like @p10
order by u.name, u.id
limit 50
