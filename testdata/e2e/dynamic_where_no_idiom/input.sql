select * /** all columns */ from person
where /*%! no 1=1 idiom */ /*%if name != null*/name = /*name*/'x'/*%end*/
/*%if age != null*/and age > /*age*/0/*%end*/
