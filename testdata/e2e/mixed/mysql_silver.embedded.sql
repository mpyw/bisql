select emp_no, name
from employees
where 1 = 1
and priority = 2
and (dept_no, region) in ((3, 'z'))

order by emp_no
limit 10
