/*%! @include audit_logs/_since.sql */
/*%if until != null*/and a.created_at < /*until*/'2026-01-01'/*%end*/
