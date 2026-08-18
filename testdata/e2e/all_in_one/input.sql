with active_depts as (
    select id from departments where /*flag*/true
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
/*%if status == 'active'*/and u.age >= 18/*%elseif status == 'pending'*/and u.age >= 13/*%else*/and u.age >= 0/*%end*/
/*%if ids != null*/and u.id in /*ids*/(0)/*%end*/
and (u.department_id, u.status) in /*pairs*/((0, 'active'))
/*%for kw in keywords : ' '*/and u.name like /*kw*/'%a%'/*%end*/
order by /*%if byName*/u.name,/*%end*/ u.id
limit /*^limit*/100
