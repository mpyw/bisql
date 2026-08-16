select * /** all columns */ from person
where 1 = 1
and name = ?
and age > ?
and id in (?, ?, ?)
