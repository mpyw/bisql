select emp_no, name
from employees
where 1 = 1
/*%! @include _frag/active.sql */
/*%if name != null*/and name = /*name*/'SCOTT'/*%end*/
order by emp_no