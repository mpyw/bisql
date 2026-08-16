with active as (
    select id from acct where flag = /*flag*/true
)
select /*+ MAX_EXECUTION_TIME(2000) */ /*%! columns are chosen at runtime */ /*%for c in cols*//*# c *//*# c_next_comma */ /*%end*/
from person p
join active a on a.id = p.id
where
/*%if name != null*/and name = /*name*/'x'/*%end*/
/*%if ids != null*/and p.id in /*ids*/(0)/*%end*/
order by /*%for s in sorts*//*# s *//*# s_next_comma */ /*%end*/
