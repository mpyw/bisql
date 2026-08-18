-- User search endpoint.
--
-- One template serves both the catalog search and the "my department" view; the only difference
-- is which optional predicates are supplied. Every optional predicate is a conjunction hung off
-- the `where 1 = 1` anchor, so an unsupplied filter drops out of the SQL entirely instead of
-- turning the query into a disjunction. Keeping it conjunctive is what lets one template back
-- both callers without the planner losing the index on the driving filter.
--
-- Anchoring, fragment by fragment:
--   - WHERE: `1 = 1` anchor; each optional predicate leads with `and`.
--   - ORDER BY: `u.id` is the stable trailing key; optional sort keys are prepended with a
--     trailing comma inside their own `/*%if*/`. A sort key is whitelisted SQL, never a bound
--     value — a column name cannot be parameterized.
--   - the keyword loop leads each iteration with `and`, off the same `1 = 1` anchor.
--   - `withDepartment` gates the projected column and its join together, so the two never
--     disagree.
select
    u.id,
    u.name,
    u.email
    /*%if withDepartment*/, d.name as department/*%end*/
from users u
/*%if withDepartment*/join departments d on d.id = u.department_id/*%end*/
where 1 = 1
/*%! @include users/fragment/scope.sql */
/*%if ageBand == 'adult'*/ and u.age >= 18/*%elseif ageBand == 'senior'*/ and u.age >= 65/*%else*/ and u.age >= 0/*%end*/
/*%if q != null*/and u.name like /*q*/'%alice%'/*%end*/
/*%if departmentIds != null*/and u.department_id in /*departmentIds*/(0)/*%end*/
/*%if tags != null*/and exists (select 1 from user_tags ut where ut.user_id = u.id and ut.tag in /*tags*/('vip'))/*%end*/
/*%for domain in emailDomains*/ and u.email like /*domain*/'%@example.com'/*%end*/
order by /*%if sortKey == 'name'*/u.name, /*%end*//*%if sortKey == 'recent'*/u.id desc, /*%end*/u.id
limit /*^ limit ?? 50 */50
