select id, name
from users
where 1 = 1
/*%! @include _frag/active.sql */
/*%if name != null*/and name = /*name*/'Alice'/*%end*/
order by id