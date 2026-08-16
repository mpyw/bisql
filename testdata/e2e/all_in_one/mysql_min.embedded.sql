with active as (
    select id from acct where flag = false
)
select p.id  /** projected columns */
from person p
join active a on a.id = p.id
where 1 = 1
