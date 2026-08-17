with active as (
    select id from acct where flag = /*flag*/true
)
select p.id, p.name
from person p
join active a on a.id = p.id
where 1 = 1
/*%if name != null*/and name = /*name*/'x'/*%end*/
/*%if ids != null*/and p.id in /*ids*/(0)/*%end*/
order by /*%if byName*/name,/*%end*/ p.id
