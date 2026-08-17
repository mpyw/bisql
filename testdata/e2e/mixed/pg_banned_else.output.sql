select id, name
from users
where 1 = 1
and age >= 0
and (department_id, status) in (($1, $2))
and name like $3
order by id
limit 5
