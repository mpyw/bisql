select * from person
where /*%if name != null*/name = /*name*/'x'/*%end*/
/*%if age != null*/and age > /*age*/0/*%end*/
