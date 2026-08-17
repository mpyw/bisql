select id, name
from users
where 1 = 1
and age >= 13
and (department_id, status) in ((3, 'banned'))

order by id
limit 10
