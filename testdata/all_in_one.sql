with active_depts as (
    select id from departments where /*flag*/true
)
select u.id, u.name
from users u
join active_depts d on d.id = u.department_id
where 1 = 1
/*%! @include _all_in_one.scope.sql */
/*%if ageBand == 'adult'*/ and u.age >= 18/*%elseif ageBand == 'senior'*/ and u.age >= 65/*%else*/ and u.age >= 0/*%end*/
/*%if ids != null*/and u.id in /*ids*/(0)/*%end*/
/*%if tags != null*/and u.tags && /*tags*/'{}'::text[]/*%end*/
and (u.department_id, u.status) in /*pairs*/((0, 'active'))
/*%for kw in keywords*/ and u.name like /*kw*/'%a%'/*%end*/
order by /*%if byName*/u.name,/*%end*/ u.id
limit /*^limit*/100
