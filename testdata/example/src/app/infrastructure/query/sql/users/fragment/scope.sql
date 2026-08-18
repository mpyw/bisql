-- Row-visibility scope shared by every users query: an optional active-only filter (its own
-- reusable fragment) and an optional single-department filter. Both are conjunctions, so the
-- scope contributes nothing when neither is requested.
/*%! @include users/fragment/active.sql */
/*%if departmentId != null*/and u.department_id = /*departmentId*/0/*%end*/
