with active_depts as (
    select id from departments where ?
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
 and u.age >= 18
and u.id in (?, ?, ?)
and (u.department_id, u.status) in ((?, ?))
and u.name like ? and u.name like ?
order by u.name, u.id
limit 50
