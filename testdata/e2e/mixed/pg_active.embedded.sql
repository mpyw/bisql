select id, name
from users
where 1 = 1
and age >= 18
and (department_id, status) in ((1, 'active'), (2, 'pending'))
and name like '%ali%' and name like '%bob%'
order by id
limit 50
