select emp_no, name
from employees
where 1 = 1
and priority = 1
and (dept_no, region) in (($1, $2), ($3, $4))
and name like $5 and name like $6
order by emp_no
limit 50
