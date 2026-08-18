select
    d.name as department,
    count(*) as events
from audit_logs a
join users u on u.id = a.user_id
join departments d on d.id = u.department_id
where 1 = 1
and u.status = $1

and a.created_at >= $2

and a.created_at < $3

group by d.name
order by events desc
