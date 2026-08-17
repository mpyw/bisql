select emp_no, name
from employees
where 1 = 1
and priority = 3
and (dept_no, region) in (($1, $2))
and name like $3
order by emp_no
limit 5
