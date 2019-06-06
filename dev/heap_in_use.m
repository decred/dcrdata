% This will continuously plot HeapInUse for a process named "dcrdata". The dcrps
% binary is required. Install it with "go get github.com/dcrlabs/dcrps".

% Try to get the GOPATH environment variable, but if it is not available
% Octave/MATLAB, it may be necessary setenv('GOPATH','/path/to/gopath') before
% calling this script, or to define the dcrps math manually here.
dcrps = [fullfile(getenv('GOPATH'), '/bin/dcrps')];
%dcrps = '/path/to/dcrps';
printf('Using dcrps at \"%s\"\n', dcrps)
heapuse = [];
h = figure;
ax = axes('parent',h);
while true,
  [s,out] = system([dcrps  ' memstats dcrdata | grep heap-in-use | awk ''{print $3}'' | sed ''s/(//''']);
  heapuse(end+1) = str2double(out);
  plot(ax, heapuse, 'linewidth', 2); drawnow
  pause(0.05)
end
