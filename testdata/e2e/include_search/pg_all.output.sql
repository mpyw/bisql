select id, name
from users
where 1 = 1
and status = $1
and department_id = $2
and name = $3
order by id