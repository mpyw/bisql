select
    d.name as department,
    count(*) as events
from audit_logs a
join users u on u.id = a.user_id
join departments d on d.id = u.department_id
where 1 = 1
/*%if activeOnly*/and u.status = /*status*/'active'/*%end*/

/*%if since != null*/and a.created_at >= /*since*/'2025-01-01'/*%end*/

/*%if until != null*/and a.created_at < /*until*/'2026-01-01'/*%end*/

group by d.name
order by events desc
