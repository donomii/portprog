require 'ripper'
require 'pp'

pp Ripper.sexp(File.read('example.rb'))
