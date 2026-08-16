select * /** all columns */ from person
where 1 = 1 /*%! optional filters */
/*%if name != null*/and name = /*name*/'x'/*%end*/
/*%if age != null*/and age > /*age*/0/*%end*/
/*%if ids != null*/and id in /*ids*/(0)/*%end*/
