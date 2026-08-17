with vip as (
    select id from departments where /*flag*/true
)
select u.id, u.name
from users u
join vip on vip.id = u.department_id
where 1 = 1
/*%if name != null*/and u.name = /*name*/'x'/*%end*/
/*%if ids != null*/and u.id in /*ids*/(0)/*%end*/
order by /*%if byName*/u.name,/*%end*/ u.id
