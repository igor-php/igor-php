<?php

function dangerous()
{
    if (rand(0, 1)) {
        die("Killed"); // ERROR
    }
    
    exit(1); // ERROR
}
