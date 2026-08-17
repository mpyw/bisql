select emp_no, name
from employees
where 1 = 1
and priority = 1
and (dept_no, region) in ((1, 'x'), (2, 'y'))
and name like '%a%' and name like '%b%'
order by emp_no
limit 50
