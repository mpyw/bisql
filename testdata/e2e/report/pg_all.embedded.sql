select
    d.name as department,
    count(*) as events
from audit_logs a
join users u on u.id = a.user_id
join departments d on d.id = u.department_id
where 1 = 1
and u.status = 'active'

and a.created_at >= '2025-01-01'

and a.created_at < '2025-12-31'

group by d.name
order by events desc
