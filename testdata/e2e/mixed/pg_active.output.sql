select id, name
from users
where 1 = 1
and age >= 18
and (department_id, status) in (($1, $2), ($3, $4))
and name like $5 and name like $6
order by id
limit 50
