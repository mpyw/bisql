select emp_no, name
from employees
where 1 = 1
and retired = $1
and dept_no = $2
and name = $3
order by emp_no