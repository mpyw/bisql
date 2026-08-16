with active as (
    select id from acct where flag = /*flag*/true /*%! active accounts only */
)
select /*%for c in cols*//*# c *//*# c_next_comma */ /*%end*/ /** projected columns */
from person p
join active a on a.id = p.id
where 1 = 1 /*%! optional filters below */
/*%if name != null*/and name = /*name*/'x'/*%end*/
/*%if ids != null*/and p.id in /*ids*/(0)/*%end*/
/*%if sorts != null*/order by /*%for s in sorts*//*# s *//*# s_next_comma */ /*%end*//*%end*/
