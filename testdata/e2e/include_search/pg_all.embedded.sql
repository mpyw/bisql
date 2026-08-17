select emp_no, name
from employees
where 1 = 1
and retired = 0
and dept_no = 10
and name = 'SCOTT'
order by emp_no