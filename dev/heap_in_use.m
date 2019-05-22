heapuse=[];
h=figure;
ax=axes('parent',h);
while true,
  [s,out] = system("dcrps memstats dcrdata | grep heap-in-use | awk '{print $3}' | sed 's/(//'");
  heapuse(end+1)=str2double(out);
  plot(ax, heapuse); drawnow
  pause(0.05)
end
