select emp_no, name
from employees
where 1 = 1
and priority = 2
and (dept_no, region) in ((?, ?))

order by emp_no
limit 10
