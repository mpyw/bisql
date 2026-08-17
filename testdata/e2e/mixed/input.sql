select emp_no, name
from employees
where 1 = 1
/*%if tier == 'gold'*/and priority = 1/*%elseif tier == 'silver'*/and priority = 2/*%else*/and priority = 3/*%end*/
and (dept_no, region) in /*pairs*/((0, 'x'))
/*%for kw in keywords : ' '*/and name like /*kw*/'%a%'/*%end*/
order by emp_no
limit /*^limit*/100
