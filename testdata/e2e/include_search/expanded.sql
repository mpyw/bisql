select emp_no, name
from employees
where 1 = 1
/*%if activeOnly*/and retired = /*zero*/0/*%end*/
/*%if dept != null*/and dept_no = /*dept*/0/*%end*/
/*%if name != null*/and name = /*name*/'SCOTT'/*%end*/
order by emp_no