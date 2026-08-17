select id, name
from users
where 1 = 1
/*%if activeOnly*/and status = /*status*/'active'/*%end*/
/*%if dept != null*/and department_id = /*dept*/0/*%end*/
/*%if name != null*/and name = /*name*/'Alice'/*%end*/
order by id