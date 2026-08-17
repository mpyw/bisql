select id, name
from users
where 1 = 1
/*%if status == 'active'*/and age >= 18/*%elseif status == 'pending'*/and age >= 13/*%else*/and age >= 0/*%end*/
and (department_id, status) in /*pairs*/((0, 'x'))
/*%for kw in keywords : ' '*/and name like /*kw*/'%a%'/*%end*/
order by id
limit /*^limit*/100
